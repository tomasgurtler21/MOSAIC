"""Tests for the session-run binding store (mosaic_logger_runstate).

Covers:
  - put_session_run_binding: first observation, idempotent repeat (start time
    unchanged, no duplicate entry), a different run_id extending the
    timeline, a run_id reappearing after an intervening run being recorded
    again, falsy session_id/run_id as a no-op, never-raises contract
  - read_session_run_timeline: file-order list; entries that are
    unparseable, non-object, missing a required key, or carrying an
    unparseable timestamp are skipped while the remaining entries survive;
    absent/unreadable/empty file returns []
  - get_session_run_binding: resolves the run active at a given instant
    across a session with several sequential runs (not merely the last
    entry written), the no-binding and before-first-entry cases, never
    raises
  - Two sessions' timelines are fully isolated: neither observes nor is
    corrupted by the other's writes or corruption

LogPaths.session_binding_dir / session_binding_entry are exercised only
incidentally here; their dedicated coverage lives in test_core_paths.py.

RED-phase note: All tests in this file will FAIL until the Stage 1
implementation exists. put_session_run_binding, read_session_run_timeline,
and get_session_run_binding do not exist yet in mosaic_logger_runstate, and
LogPaths.session_binding_dir / session_binding_entry do not exist yet in
mosaic_logger_core.
"""

import json
import os
import pathlib
import re
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_core as core
import mosaic_logger_runstate as runstate


# ---------------------------------------------------------------------------
# Shared constants
# ---------------------------------------------------------------------------

_SESSION_ID = "test-session-binding-main"
_SESSION_ID_ALT = "test-session-binding-alt"

_RUN_ID_A = "20260101T090000Z-aaaa"
_RUN_ID_B = "20260101T100000Z-bbbb"
_RUN_ID_C = "20260101T110000Z-cccc"

_T0 = "2026-01-01T09:00:00.000Z"  # run A starts
_T1 = "2026-01-01T10:00:00.000Z"  # run B starts
_T2 = "2026-01-01T11:00:00.000Z"  # run C starts

_ISO_MS_RE = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$")


def _read_timeline_raw(paths, session_id):
    """Read a binding file directly as raw parsed JSON lines (test-only helper,
    independent of read_session_run_timeline so put's on-disk shape is
    verified without relying on the function under test elsewhere)."""
    path = paths.session_binding_entry(session_id)
    if not path.exists():
        return []
    return [json.loads(ln) for ln in path.read_text("utf-8").splitlines() if ln.strip()]


# ---------------------------------------------------------------------------
# put_session_run_binding
# ---------------------------------------------------------------------------

class TestPutSessionRunBinding(unittest.TestCase):
    """put_session_run_binding: first observation, idempotence, timeline extension."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def test_returns_true_on_first_observation(self):
        result = runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T0)
        self.assertTrue(result)

    def test_first_observation_creates_exactly_one_entry(self):
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T0)
        entries = _read_timeline_raw(self.paths, _SESSION_ID)
        self.assertEqual(1, len(entries))

    def test_first_observation_records_run_id_and_start_time(self):
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T0)
        entries = _read_timeline_raw(self.paths, _SESSION_ID)
        self.assertEqual(_RUN_ID_A, entries[0]["run_id"])
        self.assertEqual(_T0, entries[0]["start_time"])

    def test_start_time_defaults_to_now_when_omitted(self):
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A)
        entries = _read_timeline_raw(self.paths, _SESSION_ID)
        self.assertRegex(entries[0]["start_time"], _ISO_MS_RE)

    def test_repeat_observation_of_same_run_id_does_not_duplicate(self):
        """Recording the same run_id twice must not add a second timeline entry."""
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T0)
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T1)
        entries = _read_timeline_raw(self.paths, _SESSION_ID)
        self.assertEqual(1, len(entries), "Repeat observation must be idempotent")

    def test_repeat_observation_does_not_shift_recorded_start_time(self):
        """The original start_time must survive a repeat observation carrying
        a different (later) timestamp."""
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T0)
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T1)
        entries = _read_timeline_raw(self.paths, _SESSION_ID)
        self.assertEqual(_T0, entries[0]["start_time"])

    def test_returns_true_when_tail_already_names_run_id(self):
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T0)
        result = runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T1)
        self.assertTrue(result)

    def test_second_sequential_run_id_extends_the_timeline(self):
        """A different run_id for the same session appends a new entry
        rather than replacing the first."""
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T0)
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_B, _T1)
        entries = _read_timeline_raw(self.paths, _SESSION_ID)
        self.assertEqual(2, len(entries))
        self.assertEqual(_RUN_ID_A, entries[0]["run_id"])
        self.assertEqual(_RUN_ID_B, entries[1]["run_id"])

    def test_run_id_reappearing_after_an_intervening_run_is_appended_again(self):
        """A, B, A: the session genuinely returned to A, so it must be
        recorded a second time rather than treated as an idempotent repeat
        of the first A entry (idempotence is against the TAIL only)."""
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T0)
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_B, _T1)
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T2)
        entries = _read_timeline_raw(self.paths, _SESSION_ID)
        self.assertEqual(3, len(entries))
        self.assertEqual([_RUN_ID_A, _RUN_ID_B, _RUN_ID_A],
                         [e["run_id"] for e in entries])

    def test_falsy_session_id_is_a_no_op_returning_false(self):
        result = runstate.put_session_run_binding(self.paths, None, _RUN_ID_A, _T0)
        self.assertFalse(result)

    def test_falsy_session_id_writes_no_entry_anywhere(self):
        runstate.put_session_run_binding(self.paths, "", _RUN_ID_A, _T0)
        d = self.paths.session_binding_dir()
        if d.exists():
            self.assertEqual([], list(d.iterdir()))

    def test_falsy_run_id_is_a_no_op_returning_false(self):
        result = runstate.put_session_run_binding(self.paths, _SESSION_ID, None, _T0)
        self.assertFalse(result)

    def test_falsy_run_id_writes_no_entry(self):
        runstate.put_session_run_binding(self.paths, _SESSION_ID, "", _T0)
        entries = _read_timeline_raw(self.paths, _SESSION_ID)
        self.assertEqual(0, len(entries))

    def test_creates_parent_directory(self):
        d = self.paths.session_binding_dir()
        self.assertFalse(d.exists(), "Pre-condition: dir must not exist yet")
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T0)
        self.assertTrue(d.exists())

    def test_returns_false_on_io_failure(self):
        """Returns False when the entry cannot be written because a regular
        file occupies the directory's own path."""
        d = self.paths.session_binding_dir()
        d.parent.mkdir(parents=True, exist_ok=True)
        d.write_bytes(b"blocker")
        result = runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T0)
        self.assertFalse(result)

    def test_never_raises_on_io_failure(self):
        d = self.paths.session_binding_dir()
        d.parent.mkdir(parents=True, exist_ok=True)
        d.write_bytes(b"blocker")
        try:
            runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T0)
        except Exception as exc:
            self.fail(f"put_session_run_binding raised unexpectedly: {exc}")

    def test_different_sessions_write_to_separate_files(self):
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T0)
        runstate.put_session_run_binding(self.paths, _SESSION_ID_ALT, _RUN_ID_B, _T0)
        entries_main = _read_timeline_raw(self.paths, _SESSION_ID)
        entries_alt = _read_timeline_raw(self.paths, _SESSION_ID_ALT)
        self.assertEqual(1, len(entries_main))
        self.assertEqual(1, len(entries_alt))
        self.assertEqual(_RUN_ID_A, entries_main[0]["run_id"])
        self.assertEqual(_RUN_ID_B, entries_alt[0]["run_id"])


# ---------------------------------------------------------------------------
# get_session_run_binding: time-based lookup
# ---------------------------------------------------------------------------

class TestGetSessionRunBinding(unittest.TestCase):
    """get_session_run_binding resolves the run active for a session at a
    given instant -- the entry with the greatest start_time <= at."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def test_resolves_to_the_only_run_when_queried_after_its_start(self):
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T0)
        result = runstate.get_session_run_binding(self.paths, _SESSION_ID, at=_T1)
        self.assertEqual(_RUN_ID_A, result)

    def test_resolves_to_the_run_active_exactly_at_its_start_time(self):
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T0)
        result = runstate.get_session_run_binding(self.paths, _SESSION_ID, at=_T0)
        self.assertEqual(_RUN_ID_A, result)

    def test_before_first_entry_resolves_to_none(self):
        """A query instant earlier than every recorded start_time is
        unresolved -- it must NOT fall back to the earliest known run."""
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T1)
        before = "2026-01-01T08:00:00.000Z"
        result = runstate.get_session_run_binding(self.paths, _SESSION_ID, at=before)
        self.assertIsNone(result)

    def test_no_binding_at_all_resolves_to_none(self):
        result = runstate.get_session_run_binding(self.paths, _SESSION_ID, at=_T0)
        self.assertIsNone(result)

    def test_resolves_earlier_run_when_queried_between_two_starts(self):
        """A session with two sequential runs must resolve a query made
        between their start times to the FIRST run, not simply the last
        entry written."""
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T0)
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_B, _T1)
        between = "2026-01-01T09:30:00.000Z"
        result = runstate.get_session_run_binding(self.paths, _SESSION_ID, at=between)
        self.assertEqual(_RUN_ID_A, result)

    def test_resolves_later_run_when_queried_after_its_start(self):
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T0)
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_B, _T1)
        result = runstate.get_session_run_binding(self.paths, _SESSION_ID, at=_T2)
        self.assertEqual(_RUN_ID_B, result)

    def test_resolves_correctly_across_three_sequential_runs(self):
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T0)
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_B, _T1)
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_C, _T2)
        self.assertEqual(_RUN_ID_A, runstate.get_session_run_binding(
            self.paths, _SESSION_ID, at="2026-01-01T09:30:00.000Z"))
        self.assertEqual(_RUN_ID_B, runstate.get_session_run_binding(
            self.paths, _SESSION_ID, at="2026-01-01T10:30:00.000Z"))
        self.assertEqual(_RUN_ID_C, runstate.get_session_run_binding(
            self.paths, _SESSION_ID, at="2026-01-01T12:00:00.000Z"))

    def test_at_defaults_to_now_when_omitted(self):
        """Without an explicit `at`, the query instant defaults to the
        current time, which is after any test-recorded (past) start_time."""
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T0)
        result = runstate.get_session_run_binding(self.paths, _SESSION_ID)
        self.assertEqual(_RUN_ID_A, result)

    def test_never_raises_on_no_binding(self):
        try:
            runstate.get_session_run_binding(self.paths, _SESSION_ID, at=_T0)
        except Exception as exc:
            self.fail(f"get_session_run_binding raised unexpectedly: {exc}")


# ---------------------------------------------------------------------------
# Resilience contract: absent, unreadable, corrupt, empty
# ---------------------------------------------------------------------------

class TestSessionBindingResilience(unittest.TestCase):
    """Absent, unreadable, corrupt, and empty binding files all degrade to an
    unresolved result and never raise, on both the read and lookup paths."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _entry_path(self, session_id=_SESSION_ID):
        return self.paths.session_binding_entry(session_id)

    # --- read_session_run_timeline ---

    def test_read_timeline_returns_empty_list_for_absent_file(self):
        result = runstate.read_session_run_timeline(self.paths, _SESSION_ID)
        self.assertEqual([], result)

    def test_read_timeline_returns_empty_list_for_empty_file(self):
        path = self._entry_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(b"")
        result = runstate.read_session_run_timeline(self.paths, _SESSION_ID)
        self.assertEqual([], result)

    def test_read_timeline_returns_empty_list_for_fully_corrupt_content(self):
        path = self._entry_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("not json at all\n{broken\n", encoding="utf-8")
        result = runstate.read_session_run_timeline(self.paths, _SESSION_ID)
        self.assertEqual([], result)

    def test_read_timeline_skips_corrupt_lines_but_keeps_usable_ones(self):
        """Partial corruption degrades gracefully: unusable lines are
        skipped, the remaining valid entries are still returned."""
        path = self._entry_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(
            json.dumps({"run_id": _RUN_ID_A, "start_time": _T0}) + "\n"
            + "{not valid json\n"
            + json.dumps({"run_id": _RUN_ID_B, "start_time": _T1}) + "\n",
            encoding="utf-8",
        )
        result = runstate.read_session_run_timeline(self.paths, _SESSION_ID)
        self.assertEqual(2, len(result))
        self.assertEqual(_RUN_ID_A, result[0]["run_id"])
        self.assertEqual(_RUN_ID_B, result[1]["run_id"])

    def test_read_timeline_skips_entries_missing_run_id(self):
        path = self._entry_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps({"start_time": _T0}) + "\n", encoding="utf-8")
        result = runstate.read_session_run_timeline(self.paths, _SESSION_ID)
        self.assertEqual([], result)

    def test_read_timeline_skips_entries_missing_start_time(self):
        path = self._entry_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps({"run_id": _RUN_ID_A}) + "\n", encoding="utf-8")
        result = runstate.read_session_run_timeline(self.paths, _SESSION_ID)
        self.assertEqual([], result)

    def test_read_timeline_skips_entries_with_unparseable_timestamp(self):
        path = self._entry_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(
            json.dumps({"run_id": _RUN_ID_A, "start_time": "not-a-timestamp"}) + "\n",
            encoding="utf-8",
        )
        result = runstate.read_session_run_timeline(self.paths, _SESSION_ID)
        self.assertEqual([], result)

    def test_read_timeline_skips_non_object_lines(self):
        path = self._entry_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(["not", "an", "object"]) + "\n", encoding="utf-8")
        result = runstate.read_session_run_timeline(self.paths, _SESSION_ID)
        self.assertEqual([], result)

    def test_read_timeline_never_raises_when_directory_occupies_the_entry_path(self):
        """A directory occupying the entry's path (instead of a file) must
        degrade to [] rather than raise."""
        path = self._entry_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        path.mkdir()  # a directory where a file is expected
        try:
            result = runstate.read_session_run_timeline(self.paths, _SESSION_ID)
        except Exception as exc:
            self.fail(f"read_session_run_timeline raised unexpectedly: {exc}")
        self.assertEqual([], result)

    # --- get_session_run_binding ---

    def test_get_binding_returns_none_for_absent_file(self):
        result = runstate.get_session_run_binding(self.paths, _SESSION_ID, at=_T0)
        self.assertIsNone(result)

    def test_get_binding_returns_none_for_empty_file(self):
        path = self._entry_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(b"")
        result = runstate.get_session_run_binding(self.paths, _SESSION_ID, at=_T0)
        self.assertIsNone(result)

    def test_get_binding_returns_none_for_fully_corrupt_content(self):
        path = self._entry_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("}{totally broken{{", encoding="utf-8")
        result = runstate.get_session_run_binding(self.paths, _SESSION_ID, at=_T0)
        self.assertIsNone(result)

    def test_get_binding_resolves_around_partially_corrupt_content(self):
        path = self._entry_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(
            "{not valid json\n"
            + json.dumps({"run_id": _RUN_ID_A, "start_time": _T0}) + "\n",
            encoding="utf-8",
        )
        result = runstate.get_session_run_binding(self.paths, _SESSION_ID, at=_T1)
        self.assertEqual(_RUN_ID_A, result)

    def test_get_binding_never_raises_when_directory_occupies_the_entry_path(self):
        path = self._entry_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        path.mkdir()
        try:
            result = runstate.get_session_run_binding(self.paths, _SESSION_ID, at=_T0)
        except Exception as exc:
            self.fail(f"get_session_run_binding raised unexpectedly: {exc}")
        self.assertIsNone(result)

    def test_get_binding_never_raises_when_session_id_is_none(self):
        try:
            result = runstate.get_session_run_binding(self.paths, None, at=_T0)
        except Exception as exc:
            self.fail(f"get_session_run_binding raised unexpectedly: {exc}")
        self.assertIsNone(result)


# ---------------------------------------------------------------------------
# Cross-session isolation
# ---------------------------------------------------------------------------

class TestSessionBindingCrossSessionIsolation(unittest.TestCase):
    """Two sessions' timelines must never observe or corrupt each other,
    because each session is keyed to its own file."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def test_two_sessions_produce_independent_timelines(self):
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T0)
        runstate.put_session_run_binding(self.paths, _SESSION_ID_ALT, _RUN_ID_B, _T0)
        timeline_main = runstate.read_session_run_timeline(self.paths, _SESSION_ID)
        timeline_alt = runstate.read_session_run_timeline(self.paths, _SESSION_ID_ALT)
        self.assertEqual(1, len(timeline_main))
        self.assertEqual(1, len(timeline_alt))
        self.assertEqual(_RUN_ID_A, timeline_main[0]["run_id"])
        self.assertEqual(_RUN_ID_B, timeline_alt[0]["run_id"])

    def test_lookup_for_one_session_is_unaffected_by_the_other_sessions_runs(self):
        runstate.put_session_run_binding(self.paths, _SESSION_ID, _RUN_ID_A, _T0)
        runstate.put_session_run_binding(self.paths, _SESSION_ID_ALT, _RUN_ID_B, _T0)
        result_main = runstate.get_session_run_binding(self.paths, _SESSION_ID, at=_T2)
        result_alt = runstate.get_session_run_binding(self.paths, _SESSION_ID_ALT, at=_T2)
        self.assertEqual(_RUN_ID_A, result_main)
        self.assertEqual(_RUN_ID_B, result_alt)

    def test_corrupting_one_sessions_file_does_not_affect_the_other(self):
        """A corrupt binding file for one session must not prevent lookup
        for an unrelated, healthy session."""
        runstate.put_session_run_binding(self.paths, _SESSION_ID_ALT, _RUN_ID_B, _T0)
        corrupt_path = self.paths.session_binding_entry(_SESSION_ID)
        corrupt_path.parent.mkdir(parents=True, exist_ok=True)
        corrupt_path.write_text("}{not json{{", encoding="utf-8")
        result_alt = runstate.get_session_run_binding(self.paths, _SESSION_ID_ALT, at=_T1)
        self.assertEqual(_RUN_ID_B, result_alt)

    def test_two_sessions_write_to_physically_different_files(self):
        path_main = self.paths.session_binding_entry(_SESSION_ID)
        path_alt = self.paths.session_binding_entry(_SESSION_ID_ALT)
        self.assertNotEqual(path_main, path_alt)


if __name__ == "__main__":
    unittest.main()
