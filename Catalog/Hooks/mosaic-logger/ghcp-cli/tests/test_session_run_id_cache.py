"""Tests for the session run_id cache in mosaic_logger_runstate (ghcp-cli variant).

Covers:
- SESSION_RUN_ID_MAX_AGE_SECONDS constant (must be 86400 / 24 hours)
- put_session_run_id: stores a run_id for a session, returns True on success,
  returns False on falsy inputs or I/O failure, writes via atomic replace,
  unconditionally replaces any prior record for that session (latest-wins),
  creates the parent directory when missing, never raises
- get_session_run_id: returns the cached run_id for a known session, returns None
  for unknown sessions, tolerates absent/empty/malformed/non-dict cache file,
  returns None when run_id field is missing or empty, evicts entries older than
  max_age_seconds but does not delete the file, never raises
- max_age_seconds parameter is injectable (no sleeping required)
- Distinct sessions cache independently
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


_RUN_ID = "20260804T150537Z-796a"
_RUN_ID_ALT = "20260804T160000Z-aabb"
_SESSION_ID = "ghcp-sess-cache-main"
_SESSION_ID_ALT = "ghcp-sess-cache-alt"


def _old_timestamp():
    """Return an ISO 8601 UTC timestamp that is definitely stale (year 2020)."""
    return "2020-01-01T00:00:00.000Z"


def _fresh_timestamp():
    now = datetime.datetime.now(datetime.timezone.utc)
    ms = now.microsecond // 1000
    return now.strftime("%Y-%m-%dT%H:%M:%S.") + f"{ms:03d}Z"


class TestSessionRunIdCacheConstant(unittest.TestCase):
    """SESSION_RUN_ID_MAX_AGE_SECONDS must be defined and set to 86400 (24 hours)."""

    def test_max_age_constant_exists(self):
        self.assertTrue(hasattr(runstate, "SESSION_RUN_ID_MAX_AGE_SECONDS"))

    def test_max_age_constant_is_86400(self):
        self.assertEqual(86400, runstate.SESSION_RUN_ID_MAX_AGE_SECONDS)


class TestPutSessionRunId(unittest.TestCase):
    """put_session_run_id persists a run_id for a session via atomic replace."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _put(self, session_id=_SESSION_ID, run_id=_RUN_ID):
        return runstate.put_session_run_id(self.paths, session_id, run_id)

    def test_returns_true_on_success(self):
        self.assertTrue(self._put())

    def test_creates_cache_file(self):
        self._put()
        path = self.paths.session_run_id_entry(_SESSION_ID)
        self.assertTrue(path.exists())

    def test_cache_file_contains_run_id(self):
        self._put(run_id=_RUN_ID)
        path = self.paths.session_run_id_entry(_SESSION_ID)
        data = json.loads(path.read_text("utf-8"))
        self.assertEqual(_RUN_ID, data["run_id"])

    def test_cache_file_contains_session_id(self):
        self._put(session_id=_SESSION_ID)
        path = self.paths.session_run_id_entry(_SESSION_ID)
        data = json.loads(path.read_text("utf-8"))
        self.assertEqual(_SESSION_ID, data["session_id"])

    def test_cache_file_contains_created_at(self):
        self._put()
        path = self.paths.session_run_id_entry(_SESSION_ID)
        data = json.loads(path.read_text("utf-8"))
        self.assertIn("created_at", data)

    def test_created_at_is_iso8601_utc_ms_format(self):
        import re
        self._put()
        path = self.paths.session_run_id_entry(_SESSION_ID)
        data = json.loads(path.read_text("utf-8"))
        pattern = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$")
        self.assertRegex(data["created_at"], pattern)

    def test_creates_parent_directory_when_missing(self):
        session_dir = self.paths.session_run_id_dir()
        self.assertFalse(session_dir.exists())
        self._put()
        self.assertTrue(session_dir.exists())

    def test_returns_false_for_none_session_id(self):
        result = runstate.put_session_run_id(self.paths, None, _RUN_ID)
        self.assertFalse(result)

    def test_returns_false_for_empty_string_session_id(self):
        result = runstate.put_session_run_id(self.paths, "", _RUN_ID)
        self.assertFalse(result)

    def test_returns_false_for_none_run_id(self):
        result = runstate.put_session_run_id(self.paths, _SESSION_ID, None)
        self.assertFalse(result)

    def test_returns_false_for_empty_string_run_id(self):
        result = runstate.put_session_run_id(self.paths, _SESSION_ID, "")
        self.assertFalse(result)

    def test_never_raises_for_none_inputs(self):
        try:
            runstate.put_session_run_id(self.paths, None, None)
        except Exception as exc:
            self.fail(f"put_session_run_id raised unexpectedly: {exc}")

    def test_latest_write_overwrites_previous_record(self):
        """Latest-wins: a second put for the same session replaces the first."""
        self._put(run_id=_RUN_ID)
        runstate.put_session_run_id(self.paths, _SESSION_ID, _RUN_ID_ALT)
        path = self.paths.session_run_id_entry(_SESSION_ID)
        data = json.loads(path.read_text("utf-8"))
        self.assertEqual(_RUN_ID_ALT, data["run_id"])

    def test_second_put_same_session_still_returns_true(self):
        self._put(run_id=_RUN_ID)
        result = runstate.put_session_run_id(self.paths, _SESSION_ID, _RUN_ID_ALT)
        self.assertTrue(result)

    def test_different_sessions_write_to_separate_files(self):
        self._put(session_id=_SESSION_ID, run_id=_RUN_ID)
        runstate.put_session_run_id(self.paths, _SESSION_ID_ALT, _RUN_ID_ALT)
        entry_main = self.paths.session_run_id_entry(_SESSION_ID)
        entry_alt = self.paths.session_run_id_entry(_SESSION_ID_ALT)
        self.assertNotEqual(entry_main, entry_alt)
        self.assertTrue(entry_main.exists())
        self.assertTrue(entry_alt.exists())


class TestGetSessionRunId(unittest.TestCase):
    """get_session_run_id reads the cached run_id for a session, respecting TTL."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _put(self, session_id=_SESSION_ID, run_id=_RUN_ID):
        runstate.put_session_run_id(self.paths, session_id, run_id)

    def _get(self, session_id=_SESSION_ID, max_age=86400):
        return runstate.get_session_run_id(
            self.paths, session_id, max_age_seconds=max_age
        )

    def test_returns_run_id_after_put(self):
        self._put()
        result = self._get()
        self.assertEqual(_RUN_ID, result)

    def test_returns_none_for_unknown_session(self):
        result = self._get(session_id="no-such-session")
        self.assertIsNone(result)

    def test_returns_none_for_none_session_id(self):
        result = runstate.get_session_run_id(self.paths, None)
        self.assertIsNone(result)

    def test_returns_none_for_empty_string_session_id(self):
        result = runstate.get_session_run_id(self.paths, "")
        self.assertIsNone(result)

    def test_returns_none_when_file_absent(self):
        result = self._get()
        self.assertIsNone(result)

    def test_returns_none_for_empty_file(self):
        path = self.paths.session_run_id_entry(_SESSION_ID)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(b"")
        result = self._get()
        self.assertIsNone(result)

    def test_returns_none_for_malformed_json(self):
        path = self.paths.session_run_id_entry(_SESSION_ID)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("this is not json", encoding="utf-8")
        result = self._get()
        self.assertIsNone(result)

    def test_returns_none_when_json_is_not_a_dict(self):
        path = self.paths.session_run_id_entry(_SESSION_ID)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text('["run_id", "20260101T000000Z-1234"]', encoding="utf-8")
        result = self._get()
        self.assertIsNone(result)

    def test_returns_none_when_run_id_field_missing(self):
        path = self.paths.session_run_id_entry(_SESSION_ID)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps({
            "session_id": _SESSION_ID,
            "created_at": _fresh_timestamp(),
        }), encoding="utf-8")
        result = self._get()
        self.assertIsNone(result)

    def test_returns_none_when_run_id_field_is_empty_string(self):
        path = self.paths.session_run_id_entry(_SESSION_ID)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps({
            "session_id": _SESSION_ID,
            "run_id": "",
            "created_at": _fresh_timestamp(),
        }), encoding="utf-8")
        result = self._get()
        self.assertIsNone(result)

    def test_returns_none_for_stale_entry(self):
        """An entry with a far-past created_at is not returned."""
        path = self.paths.session_run_id_entry(_SESSION_ID)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps({
            "session_id": _SESSION_ID,
            "run_id": _RUN_ID,
            "created_at": _old_timestamp(),
        }), encoding="utf-8")
        result = self._get(max_age=120)  # 120 s max_age; year-2020 entry is stale
        self.assertIsNone(result)

    def test_fresh_entry_returned_within_max_age(self):
        self._put()
        result = self._get(max_age=86400)
        self.assertEqual(_RUN_ID, result)

    def test_max_age_zero_evicts_all_entries(self):
        """max_age_seconds=0 means every entry is immediately stale."""
        self._put()
        result = self._get(max_age=0)
        self.assertIsNone(result)

    def test_stale_read_does_not_delete_or_truncate_file(self):
        """Staleness is not an error; the file is left intact after a stale read."""
        path = self.paths.session_run_id_entry(_SESSION_ID)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps({
            "session_id": _SESSION_ID,
            "run_id": _RUN_ID,
            "created_at": _old_timestamp(),
        }), encoding="utf-8")
        self._get(max_age=120)
        self.assertTrue(path.exists())
        self.assertGreater(path.stat().st_size, 0)

    def test_never_raises_on_absent_file(self):
        try:
            runstate.get_session_run_id(self.paths, _SESSION_ID)
        except Exception as exc:
            self.fail(f"get_session_run_id raised on absent file: {exc}")

    def test_never_raises_on_malformed_file(self):
        path = self.paths.session_run_id_entry(_SESSION_ID)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("}{totally broken{{", encoding="utf-8")
        try:
            runstate.get_session_run_id(self.paths, _SESSION_ID)
        except Exception as exc:
            self.fail(f"get_session_run_id raised on malformed file: {exc}")

    def test_never_raises_on_none_session_id(self):
        try:
            runstate.get_session_run_id(self.paths, None)
        except Exception as exc:
            self.fail(f"get_session_run_id raised for None session_id: {exc}")

    def test_different_sessions_return_their_respective_run_ids(self):
        runstate.put_session_run_id(self.paths, _SESSION_ID, _RUN_ID)
        runstate.put_session_run_id(self.paths, _SESSION_ID_ALT, _RUN_ID_ALT)
        result_main = runstate.get_session_run_id(self.paths, _SESSION_ID)
        result_alt = runstate.get_session_run_id(self.paths, _SESSION_ID_ALT)
        self.assertEqual(_RUN_ID, result_main)
        self.assertEqual(_RUN_ID_ALT, result_alt)


if __name__ == "__main__":
    unittest.main()
